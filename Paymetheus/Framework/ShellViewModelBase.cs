using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Threading.Tasks;

namespace Paymetheus.Framework
{
    public class ShellViewModelBase : ViewModelBase
    {
        private string _windowTitle;
        public string WindowTitle
        {
            get { return _windowTitle; }
            set { _windowTitle = value; RaisePropertyChanged(); }
        }

        private DialogViewModelBase _visibleDialogContent;
        public DialogViewModelBase VisibleDialogContent
        {
            get { return _visibleDialogContent; }
            set { _visibleDialogContent = value; RaisePropertyChanged(); }
        }

        public void ShowDialog(DialogViewModelBase dialog)
        {
            if (VisibleDialogContent != null)
            {
                return;
            }

            VisibleDialogContent = dialog;
        }

        public void HideDialog()
        {
            VisibleDialogContent = null;
        }
    }
}
